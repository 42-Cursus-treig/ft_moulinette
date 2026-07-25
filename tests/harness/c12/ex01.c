#include "ft_list.h"
#include <unistd.h>

void	ft_list_push_front(t_list **begin_list, void *data);

static void	put_str(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
}

static void	print_list_str(t_list *lst)
{
	while (lst)
	{
		put_str((char *)lst->data);
		write(1, "\n", 1);
		lst = lst->next;
	}
}

int	main(int argc, char **argv)
{
	t_list	*begin;
	int		i;

	begin = NULL;
	i = 1;
	while (i < argc)
	{
		ft_list_push_front(&begin, argv[i]);
		i++;
	}
	print_list_str(begin);
	return (0);
}
