angular.module("irlog", ['ngRoute', 'angular-growl'])

.config(['$routeProvider', 'growlProvider', function($routeProvider, growlProvider) {
    $routeProvider
        .when('/', {
            controller: 'MainCtrl',
            templateUrl: 'templates/list.html'
        })
        ;

    growlProvider.onlyUniqueMessages(false);
    growlProvider.globalTimeToLive(5000);
    growlProvider.globalDisableCountDown(true);
}])

.controller("MainCtrl", ['$scope', '$http', 'growl', function($scope, $http, growl) {
    $http.get('/api/logs')
        .success(function(data, status, headers, config) {
            $scope.logs = data;
        })
        .error(function(data, status, headers, config) {
            console.log("err");
        })
        ;

    $scope.post = function(log) {
        $http.post('/api/log/' + log.id + '/message', {}, {})
            .success(function(data, status, headers, config) {
                growl.success("success to post");
            })
            .error(function(data, status, headers, config) {
                growl.error("failed to post");
            })
            ;
    };

    {
        var ordinal;

        $scope.modal = function(log) {
            ordinal = angular.copy(log)
            $scope.log = log;
            $scope.showForm.$setPristine();
            $('#showModal').modal('show');
        };

        $scope.$on('modalHidden', function(e) {
            if ($scope.showForm.$dirty) {
                angular.forEach($scope.logs, function(log, i) {
                    if (log.id == ordinal.id) {
                        $scope.logs[i] = ordinal;
                        $scope.$apply();
                    }
                });
            }
        });
    }
}])

.controller("ShowCtrl", ['$scope', '$http', 'growl', function($scope, $http, growl) {

    $scope.update = function() {

        if ($scope.log === null) {
            growl.error("error");
            return;
        }

        if (!$scope.showForm.$valid) {
            return;
        }

        // http://stackoverflow.com/questions/11442632/how-can-i-post-data-as-form-data-instead-of-a-request-payload/11443066#11443066
        $http.post(
            '/api/log/' + $scope.log.id,
            $.param({ name: $scope.log.name }),
            { headers:  {'Content-Type': 'application/x-www-form-urlencoded'}})
            .success(function(data, status, headers, config) {
                $scope.log = data;
                $scope.showForm.$setPristine();
                $('#showModal').modal('hide');
                growl.success("success to update");
            })
            .error(function(data, status, headers, config) {
                growl.error("failed to update");
            })
            ;

    };

    $('#showModal').on('hidden.bs.modal', function(e) {
        $scope.$emit('modalHidden', e);
    });
}])

.directive("irlogShowModal", [function() {
    return {
        restrict: "E",
        templateUrl: "templates/show_modal.html",
        controller: 'ShowCtrl'
    }
}])

;
